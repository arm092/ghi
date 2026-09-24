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
            if(parameters.getPosition().getNode().getElementType()==GhiLexer.STRING||parameters.getPosition().getNode().getElementType()==GhiLexer.COMMENT)return;
            for(var symbol:GhiSymbols.forFile(file).complete(file,parameters.getOffset())){
                var item=LookupElementBuilder.create(symbol.name).withTypeText(symbol.kind+(symbol.type.isEmpty()?"":" "+symbol.type),true).withIcon(GhiIcons.FILE);
                if(symbol.kind.equals("func"))item=item.withTailText("("+String.join(", ",symbol.parameters)+")",true);
                result.addElement(item);
            }
        }
    });}
}
