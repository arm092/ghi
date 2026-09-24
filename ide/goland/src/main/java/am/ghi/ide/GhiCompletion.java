package am.ghi.ide;
import com.intellij.codeInsight.completion.*;
import com.intellij.codeInsight.lookup.LookupElementBuilder;
import com.intellij.patterns.PlatformPatterns;
import com.intellij.util.ProcessingContext;
import org.jetbrains.annotations.NotNull;
public final class GhiCompletion extends CompletionContributor {
    public GhiCompletion(){extend(CompletionType.BASIC,PlatformPatterns.psiElement().withLanguage(GhiLanguage.INSTANCE),new CompletionProvider<>(){
        @Override protected void addCompletions(@NotNull CompletionParameters parameters,@NotNull ProcessingContext context,@NotNull CompletionResultSet result){
            var file=parameters.getPosition().getContainingFile();
            if(parameters.getPosition().getNode().getElementType()==GhiLexer.COMMENT)return;
            if(parameters.getPosition().getNode().getElementType()==GhiLexer.STRING){
                int start=parameters.getPosition().getTextOffset();String before=file.getText().substring(0,start);
                if(!before.matches("(?s).*\\bimport\\s+(?:[\\p{L}_][\\p{L}\\p{N}_]*\\s+)?"))return;
                String prefix=file.getText().substring(start+1,Math.max(start+1,parameters.getOffset()));
                for(String path:GhiGoSymbols.importPaths(file,prefix))result.withPrefixMatcher(prefix).addElement(LookupElementBuilder.create(path).withTypeText("Go SDK package"));
                return;
            }
            String preceding=file.getText().substring(0,parameters.getPosition().getTextOffset()).stripTrailing();
            if(!preceding.endsWith(".")&&!preceding.matches("(?s).*\\bnew")&&!parameters.getPosition().getText().isEmpty() && "new".startsWith(parameters.getPosition().getText().replace("IntellijIdeaRulezzz", "")))result.addElement(LookupElementBuilder.create("new").bold());
            var available=GhiSymbols.forFile(file).complete(file,parameters.getOffset());
            if(!preceding.endsWith(".")&&!preceding.substring(preceding.lastIndexOf('\n')+1).stripLeading().startsWith("import ")){
                for(var choice:GhiTypeImports.candidates(file,null)){
                    if(available.stream().anyMatch(symbol->symbol==choice.symbol()))continue;
                    if(preceding.matches("(?s).*\\bnew")&&!choice.symbol().kind.equals("class"))continue;
                    result.addElement(LookupElementBuilder.create(choice,choice.symbol().name).withTailText(" ("+choice.path()+")",true).withIcon(GhiIcons.FILE)
                        .withInsertHandler((insertion,item)->GhiTypeImports.apply(insertion.getFile(),insertion.getEditor(),
                            new GhiTypeImports.Site(insertion.getStartOffset(),insertion.getTailOffset(),choice.symbol().name),choice)));
                }
            }
            for(var symbol:available){
                var item=LookupElementBuilder.create(symbol.name).withTypeText(symbol.kind+(symbol.type.isEmpty()?"":" "+symbol.type),true).withIcon(GhiIcons.FILE);
                if(symbol.kind.equals("func")||symbol.callable)item=item.withTailText("("+String.join(", ",symbol.parameters)+")",true);
                result.addElement(item);
            }
        }
    });}
}
