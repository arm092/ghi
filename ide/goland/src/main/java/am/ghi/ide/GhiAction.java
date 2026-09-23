package am.ghi.ide;
import com.intellij.execution.RunContentExecutor;
import com.intellij.execution.process.*;
import com.intellij.openapi.actionSystem.*;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.progress.*;
import com.intellij.openapi.project.DumbAwareAction;
import com.intellij.openapi.ui.Messages;
import java.nio.file.Files;
abstract class GhiAction extends DumbAwareAction {
    private final String operation;
    GhiAction(String operation){this.operation=operation;}
    public ActionUpdateThread getActionUpdateThread(){return ActionUpdateThread.BGT;}
    public void update(AnActionEvent event){event.getPresentation().setEnabled(event.getProject()!=null);}
    public void actionPerformed(AnActionEvent event){
        var project=event.getProject();if(project==null)return;
        FileDocumentManager.getInstance().saveAllDocuments();
        var state=GhiSettings.get(project).getState();
        final com.intellij.execution.configurations.GeneralCommandLine command;
        final java.nio.file.Path directory;
        try{
            directory=GhiCommand.directory(project.getBasePath(),state.directory);
            if(!Files.isDirectory(directory))throw new IllegalArgumentException("Project directory does not exist: "+directory);
            command=GhiCommand.create(GhiCommand.executable(state.executable),directory,operation,state.arguments);
        }catch(RuntimeException error){Messages.showErrorDialog(project,error.getMessage(),"Ghi Settings");return;}
        ProgressManager.getInstance().run(new Task.Backgroundable(project,"Ghi "+operation,false){
            public void run(ProgressIndicator indicator){
                try{
                    var handler=new KillableColoredProcessHandler(command);
                    ProcessTerminatedListener.attach(handler,project);
                    ApplicationManager.getApplication().invokeLater(()->{
                        if(project.isDisposed()){handler.destroyProcess();return;}
                        new RunContentExecutor(project,handler).withTitle("Ghi "+operation)
                            .withFilter(new GhiConsoleFilter(project,directory)).run();
                    });
                }catch(Exception error){ApplicationManager.getApplication().invokeLater(()->{
                    if(!project.isDisposed())Messages.showErrorDialog(project,
                        error.getMessage()+"\nConfigure the compiler under Settings > Languages & Frameworks > Ghi.","Ghi "+operation);
                });}
            }
        });
    }
    public static final class Build extends GhiAction {public Build(){super("build");}}
    public static final class Run extends GhiAction {public Run(){super("run");}}
}
